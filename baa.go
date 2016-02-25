package baa

import (
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

const (
	// DEV mode
	DEV = "development"
	// PROD mode
	PROD = "production"
	// TEST mode
	TEST = "test"
)

// Baa provlider an application
type Baa struct {
	debug           bool
	name            string
	di              *DI
	router          *Router
	pool            sync.Pool
	errorHandler    ErrorHandleFunc
	notFoundHandler HandlerFunc
	middleware      []HandlerFunc
}

// Middleware middleware handler
type Middleware interface{}

// HandlerFunc context handler
type HandlerFunc func(*Context)

// ErrorHandleFunc HTTP error handleFunc
type ErrorHandleFunc func(error, *Context)

// New create a baa application without any config.
func New() *Baa {
	b := new(Baa)
	b.middleware = make([]HandlerFunc, 0)
	b.pool = sync.Pool{
		New: func() interface{} {
			return newContext(nil, nil, b)
		},
	}
	if os.Getenv("Baa.Env") != PROD {
		b.debug = true
	}
	b.di = newDI()
	b.router = newRouter()
	b.errorHandler = b.DefaultErrorHandler
	b.notFoundHandler = b.DefaultNotFoundHandler
	b.SetDI("logger", log.New(os.Stderr, "[Baa] ", log.LstdFlags))
	b.SetDI("render", newRender())
	return b
}

// Server returns the internal *http.Server.
func (b *Baa) Server(addr string) *http.Server {
	s := &http.Server{Addr: addr}
	return s
}

// Run runs a server.
func (b *Baa) Run(addr string) {
	b.run(b.Server(addr))
}

// RunTLS runs a server with TLS configuration.
func (b *Baa) RunTLS(addr, certfile, keyfile string) {
	b.run(b.Server(addr), certfile, keyfile)
}

// RunServer runs a custom server.
func (b *Baa) RunServer(s *http.Server) {
	b.run(s)
}

// RunTLSServer runs a custom server with TLS configuration.
func (b *Baa) RunTLSServer(s *http.Server, crtFile, keyFile string) {
	b.run(s, crtFile, keyFile)
}

func (b *Baa) run(s *http.Server, files ...string) {
	s.Handler = b
	if len(files) == 0 {
		b.Logger().Printf("Listen %s", s.Addr)
		b.Logger().Fatal(s.ListenAndServe())
	} else if len(files) == 2 {
		b.Logger().Printf("Listen %s with TLS", s.Addr)
		b.Logger().Fatal(s.ListenAndServeTLS(files[0], files[1]))
	} else {
		b.Logger().Fatal("invalid TLS configuration")
	}
}

func (b *Baa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := b.pool.Get().(*Context)
	c.reset(w, r)

	// build handler chain
	c.handlers = append(c.handlers, b.middleware...)

	route := b.router.lookup(r.URL.Path, b.router.routeMap[methodKeys[r.Method]], c)
	// notFound
	if route == nil || route.handlers == nil {
		c.handlers = append(c.handlers, b.notFoundHandler)
	} else {
		c.handlers = append(c.handlers, route.handlers...)
	}

	c.Next()

	b.pool.Put(c)
}

// SetDebug set baa debug
func (b *Baa) SetDebug(v bool) {
	b.debug = v
}

// Logger return baa logger
func (b *Baa) Logger() Logger {
	return b.GetDI("logger").(Logger)
}

// Render return baa render
func (b *Baa) Render() Renderer {
	return b.GetDI("render").(Renderer)
}

// SetErrorHandler registers a custom Baa.ErrorHandleFunc.
func (b *Baa) SetErrorHandler(h ErrorHandleFunc) {
	b.errorHandler = h
}

// Use registers a middleware
func (b *Baa) Use(m ...Middleware) {
	for i := range m {
		b.middleware = append(b.middleware, wrapMiddleware(m[i]))
	}
}

// SetDI registers a dependency injection
func (b *Baa) SetDI(name string, h interface{}) {
	b.di.set(name, h)
}

// GetDI fetch a registered dependency injection
func (b *Baa) GetDI(name string) interface{} {
	return b.di.get(name)
}

// Static set static file route
// h used for set Expries ...
func (b *Baa) Static(prefix string, dir string, index bool, h HandlerFunc) {
	if prefix == "" {
		panic("baa.Static prefix can not be empty")
	}
	if dir == "" {
		panic("baa.Static dir can not be empty")
	}
	staticHandler := newStatic(prefix, dir, index, h)
	b.Get(prefix, staticHandler)
	b.Get(prefix+":file", staticHandler)
}

// SetAutoHead sets the value who determines whether add HEAD method automatically
// when GET method is added. Combo router will not be affected by this value.
func (b *Baa) SetAutoHead(v bool) {
	b.router.autoHead = v
}

// Route is a shortcut for same handlers but different HTTP methods.
//
// Example:
// 		baa.route("/", "GET,POST", h)
func (b *Baa) Route(pattern, methods string, h ...HandlerFunc) *Route {
	var ru *Route
	for _, m := range strings.Split(methods, ",") {
		ru = b.router.add(strings.TrimSpace(m), pattern, h)
	}
	return ru
}

// Group registers a list of same prefix route
func (b *Baa) Group(pattern string, f func(), h ...HandlerFunc) {
	b.router.groupAdd(pattern, f, h)
}

// Get is a shortcut for b.router.handle("GET", pattern, handlers)
func (b *Baa) Get(pattern string, h ...HandlerFunc) *Route {
	rs := b.router.add("GET", pattern, h)
	if b.router.autoHead {
		b.Head(pattern, h...)
	}
	return rs
}

// Patch is a shortcut for b.router.handle("PATCH", pattern, handlers)
func (b *Baa) Patch(pattern string, h ...HandlerFunc) *Route {
	return b.router.add("PATCH", pattern, h)
}

// Post is a shortcut for b.router.handle("POST", pattern, handlers)
func (b *Baa) Post(pattern string, h ...HandlerFunc) *Route {
	return b.router.add("POST", pattern, h)
}

// Put is a shortcut for b.router.handle("PUT", pattern, handlers)
func (b *Baa) Put(pattern string, h ...HandlerFunc) *Route {
	return b.router.add("PUT", pattern, h)
}

// Delete is a shortcut for b.router.handle("DELETE", pattern, handlers)
func (b *Baa) Delete(pattern string, h ...HandlerFunc) *Route {
	return b.router.add("DELETE", pattern, h)
}

// Options is a shortcut for b.router.handle("OPTIONS", pattern, handlers)
func (b *Baa) Options(pattern string, h ...HandlerFunc) *Route {
	return b.router.add("OPTIONS", pattern, h)
}

// Head is a shortcut for b.router.handle("HEAD", pattern, handlers)
func (b *Baa) Head(pattern string, h ...HandlerFunc) *Route {
	return b.router.add("HEAD", pattern, h)
}

// Any is a shortcut for b.router.handle("*", pattern, handlers)
func (b *Baa) Any(pattern string, h ...HandlerFunc) *Route {
	var ru *Route
	for m := range methods {
		ru = b.router.add(m, pattern, h)
	}
	return ru
}

// NotFound set not found route handler
func (b *Baa) NotFound(h HandlerFunc) {
	b.notFoundHandler = h
}

// Error execute internal error handler
func (b *Baa) Error(err error, c *Context) {
	if b.errorHandler != nil {
		b.errorHandler(err, c)
		return
	}
	http.Error(c.Resp, err.Error(), 500)
	if b.debug {
		b.Logger().Println("Error " + err.Error())
	}
}

// DefaultErrorHandler invokes the default HTTP error handler.
func (b *Baa) DefaultErrorHandler(err error, c *Context) {
	code := http.StatusInternalServerError
	msg := http.StatusText(code)
	if b.debug {
		msg = err.Error()
	}
	http.Error(c.Resp, msg, code)
}

// DefaultNotFoundHandler invokes the default HTTP error handler.
func (b *Baa) DefaultNotFoundHandler(c *Context) {
	code := http.StatusNotFound
	msg := http.StatusText(code)
	http.Error(c.Resp, msg, code)
}

// URLFor use named route return format url
func (b *Baa) URLFor(name string, args ...interface{}) string {
	return b.router.urlFor(name, args...)
}

// wrapMiddleware wraps middleware.
func wrapMiddleware(m Middleware) HandlerFunc {
	switch m := m.(type) {
	case HandlerFunc:
		return m
	case func(*Context):
		return m
	case http.Handler:
		return wrapHandlerFunc(func(c *Context) {
			m.ServeHTTP(c.Resp, c.Req)
		})
	case http.HandlerFunc:
		return wrapHandlerFunc(func(c *Context) {
			m(c.Resp, c.Req)
		})
	case func(http.ResponseWriter, *http.Request):
		return wrapHandlerFunc(func(c *Context) {
			m(c.Resp, c.Req)
		})
	default:
		panic("unknown middleware")
	}
}
