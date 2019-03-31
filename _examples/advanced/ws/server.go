package ws

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/kataras/fastws"
)

type Server struct {
	mu         sync.RWMutex
	NSAcceptor NSAcceptor
	namespaces Namespaces

	// connections chan *conn
	ws    *fastws.FastWS
	count uint64

	connections map[*conn]struct{}
	connect     chan *conn
	disconnect  chan *conn
	broadcast   chan []byte

	OnError      func(c Conn, err error) bool
	OnConnect    func(c Conn) error
	OnDisconnect func(c Conn)
}

func New(connHandler connHandler) *Server {
	ws := fastws.New()
	s := &Server{
		namespaces: connHandler.getNamespaces(),

		connections: make(map[*conn]struct{}),
		connect:     make(chan *conn, 1),
		disconnect:  make(chan *conn),

		// connections: make(chan *conn, 1),
		ws:         ws,
		NSAcceptor: DefaultNSAcceptor,
	}

	ws.OnConnected = s.onConnected
	go s.start()

	return s
}

func (s *Server) SetIDGenerator(gen func(*http.Request) string) {
	if gen == nil {
		s.ws.IDGenerator = fastws.DefaultIDGenerator
	}

	s.ws.IDGenerator = func(c *fastws.Conn) string {
		return gen(c.Request)
	}
}

func (s *Server) start() {
	for {
		select {
		case c := <-s.connect:
			s.connections[c] = struct{}{}
			atomic.AddUint64(&s.count, 1)
		case c := <-s.disconnect:
			if _, ok := s.connections[c]; ok {
				delete(s.connections, c)
				close(c.out)
				atomic.AddUint64(&s.count, ^uint64(0))
				if s.OnDisconnect != nil {
					s.OnDisconnect(c)
				}
			}
		case b := <-s.broadcast:
			for c := range s.connections {
				select {
				case c.out <- b:
				default:
					close(c.out)
					delete(s.connections, c)
					atomic.AddUint64(&s.count, ^uint64(0))
				}
			}
		}
	}
}

func (s *Server) Close() error {
	// TODO:
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.ws.UpgradeHTTP(w, r)
}

func (s *Server) GetTotalConnections() uint64 {
	return atomic.LoadUint64(&s.count)
}

var ErrBadNamespace = errors.New("bad namespace")
var ErrForbiddenNamespace = errors.New("forbidden namespace")

func (s *Server) onConnected(conn *fastws.Conn) error {
	// namespace := conn.Request.URL.Query().Get("ns")
	// if !s.NSAcceptor(conn.Request, namespace) {
	// 	return ErrForbiddenNamespace
	// }

	// events, ok := s.namespaces[namespace]
	// if !ok {
	// 	return ErrBadNamespace
	// }

	c := newConn(conn, s.namespaces)
	c.server = s

	//	nsConn := c.getNSConnection(namespace)

	if s.OnError != nil {
		conn.OnError = func(err error) bool {
			if fastws.IsDisconnected(err) {
				return false
			}

			return s.OnError(c, err)
		}
	}

	s.connect <- c
	go c.startWriter()
	go c.startReader()

	if s.OnConnect != nil {
		if err := s.OnConnect(c); err != nil {
			return err
		}
	}

	// events.fireOnNamespaceConnect(c, Message{Namespace: namespace, isConnect: true})

	return nil
}