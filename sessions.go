// Copyright 2016 The Gem Authors. All rights reserved.
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

package sessions

import (
	"encoding/gob"
	"fmt"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

// Default flashes key.
const flashesKey = "_flash"

// Options

// Options stores configuration for a session or session store.
//
// Fields are a subset of http.Cookie fields.
type Options struct {
	Path   string
	Domain string
	// MaxAge=0 means no 'Max-Age' attribute specified.
	// MaxAge<0 means delete cookie now, equivalently 'Max-Age: 0'.
	// MaxAge>0 means Max-Age attribute present and given in seconds.
	MaxAge   int
	Secure   bool
	HttpOnly bool
}

// Session

// NewSession is called by session stores to create a new session instance.
func NewSession(store Store, name string) *Session {
	return &Session{
		Values: make(map[interface{}]interface{}),
		store:  store,
		name:   name,
	}
}

// Session stores the values and optional configuration for a session.
type Session struct {
	// The ID of the session, generated by stores. It should not be used for
	// user data.
	ID string
	// Values contains the user-data for the session.
	Values  map[interface{}]interface{}
	Options *Options
	IsNew   bool
	store   Store
	name    string
}

// Flashes returns a slice of flash messages from the session.
//
// A single variadic argument is accepted, and it is optional: it defines
// the flash key. If not defined "_flash" is used by default.
func (s *Session) Flashes(vars ...string) []interface{} {
	var flashes []interface{}
	key := flashesKey
	if len(vars) > 0 {
		key = vars[0]
	}
	if v, ok := s.Values[key]; ok {
		// Drop the flashes and return it.
		delete(s.Values, key)
		flashes = v.([]interface{})
	}
	return flashes
}

// AddFlash adds a flash message to the session.
//
// A single variadic argument is accepted, and it is optional: it defines
// the flash key. If not defined "_flash" is used by default.
func (s *Session) AddFlash(value interface{}, vars ...string) {
	key := flashesKey
	if len(vars) > 0 {
		key = vars[0]
	}
	var flashes []interface{}
	if v, ok := s.Values[key]; ok {
		flashes = v.([]interface{})
	}
	s.Values[key] = append(flashes, value)
}

// Save is a convenience method to save this session. It is the same as calling
// store.Save(request, response, session). You should call Save before writing to
// the response or returning from the handler.
func (s *Session) Save(ctx *fasthttp.RequestCtx) error {
	return s.store.Save(ctx, s)
}

// Name returns the name used to register the session.
func (s *Session) Name() string {
	return s.name
}

// Store returns the session store used to register the session.
func (s *Session) Store() Store {
	return s.store
}

// Registry

// sessionInfo stores a session tracked by the registry.
type sessionInfo struct {
	s *Session
	e error
}

var (
	registryPool = &sync.Pool{
		New: func() interface{} {
			return &Registry{}
		},
	}
)

// GetRegistry returns a registry instance for the current request.
func GetRegistry(ctx *fasthttp.RequestCtx) (registry *Registry) {
	if registry = Get(ctx); registry != nil {
		return registry
	}
	registry = registryPool.Get().(*Registry)
	registry.ctx = ctx
	registry.sessions = make(map[string]sessionInfo)
	Set(ctx, registry)
	return
}

// Registry stores sessions used during a request.
type Registry struct {
	ctx      *fasthttp.RequestCtx
	sessions map[string]sessionInfo
}

// Get registers and returns a session for the given name and session store.
//
// It returns a new session if there are no sessions registered for the name.
func (r *Registry) Get(store Store, name string) (session *Session, err error) {
	if !isCookieNameValid(name) {
		return nil, fmt.Errorf("sessions: invalid character in cookie name: %s", name)
	}
	if info, ok := r.sessions[name]; ok {
		session, err = info.s, info.e
	} else {
		session, err = store.New(r.ctx, name)
		session.name = name
		r.sessions[name] = sessionInfo{s: session, e: err}
	}
	session.store = store
	return
}

// Save saves all sessions registered for the current request.
func (r *Registry) Save() error {
	var errMulti MultiError
	for name, info := range r.sessions {
		session := info.s
		if session.store == nil {
			errMulti = append(errMulti, fmt.Errorf(
				"sessions: missing store for session %q", name))
		} else if err := session.store.Save(r.ctx, session); err != nil {
			errMulti = append(errMulti, fmt.Errorf(
				"sessions: error saving session %q -- %v", name, err))
		}
	}
	if errMulti != nil {
		return errMulti
	}
	return nil
}

// close put the registry instance into pool for reusing.
func (r *Registry) close() {
	r.ctx = nil
	registryPool.Put(r)
}

// Helpers

func init() {
	gob.Register([]interface{}{})
}

// Save saves all sessions used during the current request.
func Save(ctx *fasthttp.RequestCtx) error {
	return GetRegistry(ctx).Save()
}

// NewCookie returns an pointer of fasthttp.Cookie with the options set.
// It also sets the Expires field calculated based on the MaxAge value,
// for Internet Explorer compatibility.
func NewCookie(name, value string, options *Options) *fasthttp.Cookie {
	cookie := &fasthttp.Cookie{}
	cookie.SetKey(name)
	cookie.SetValue(value)
	cookie.SetPath(options.Path)
	cookie.SetDomain(options.Domain)
	cookie.SetHTTPOnly(options.HttpOnly)
	cookie.SetSecure(options.Secure)

	if options.MaxAge > 0 {
		d := time.Duration(options.MaxAge) * time.Second
		cookie.SetExpire(time.Now().Add(d))
	} else if options.MaxAge < 0 {
		// Set it to the past to expire now.
		cookie.SetExpire(time.Unix(1, 0))
	}
	return cookie
}

// Error

// MultiError stores multiple errors.
//
// Borrowed from the App Engine SDK.
type MultiError []error

func (m MultiError) Error() string {
	s, n := "", 0
	for _, e := range m {
		if e != nil {
			if n == 0 {
				s = e.Error()
			}
			n++
		}
	}
	switch n {
	case 0:
		return "(0 errors)"
	case 1:
		return s
	case 2:
		return s + " (and 1 other error)"
	}
	return fmt.Sprintf("%s (and %d other errors)", s, n-1)
}
