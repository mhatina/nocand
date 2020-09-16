package socket

import (
	"fmt"
	"github.com/omzlo/clog"
	"io"
	"net"
	"sync"
)

/****************************************************************************/

// Client represents a single connection from an external client through TCP/IP
//
//
type Client struct {
	Id              uint
	Server          *Server
	Conn            net.Conn
	OutputChan      chan *Event
	TerminationChan chan bool
	Subscriptions   *SubscriptionList
	Authenticated   bool
	Connected       bool
	Next            *Client
}

func (c *Client) String() string {
	return fmt.Sprintf("%d (%s)", c.Id, c.Conn.RemoteAddr())
}

func (c *Client) Put(eid EventId, value interface{}) error {
	if !c.Connected {
		return fmt.Errorf("Put failed, client %s is not connected", c)
	}
	c.OutputChan <- NewEvent(eid, value)
	return nil
}

func (c *Client) Get() (EventId, []byte, error) {
	if !c.Connected {
		return NoEvent, nil, fmt.Errorf("Get failed, client %s is not connected", c)
	}

	eid, value, err := DecodeFromStream(c.Conn)
	if err != nil {
		//c.TerminationChan <- true
		return NoEvent, nil, err
	}
	return eid, value, err
}

func clientAuthHandler(c *Client, eid EventId, value []byte) error {
	var auth Authenticator

	if err := auth.UnpackValue(value); err != nil {
		return err
	}
	if string(auth) == c.Server.AuthToken {
		c.Authenticated = true
		clog.Info("Client %s successfully authenticated", c)
		return c.Put(ServerAckEvent, SERVER_ACK_SUCCESS)
	}
	clog.Info("Client %s failed to authenticate using key '%s'", c, auth)
	return c.Put(ServerAckEvent, SERVER_ACK_UNAUTHORIZED)
}

func clientSubscribeHandler(c *Client, eid EventId, value []byte) error {
	sl := NewSubscriptionList()

	if err := sl.UnpackValue(value); err != nil {
		return err
	}
	c.Subscriptions = sl
	return c.Put(ServerAckEvent, SERVER_ACK_SUCCESS)
}

func clientHelloHandler(c *Client, eid EventId, value []byte) error {
	return c.Put(ServerHelloEvent, []byte{'E', 'M', 1, 0})
}

/****************************************************************************/

// Server
//
//

type EventHandler func(*Client, EventId, []byte) error

type Server struct {
	Mutex     sync.Mutex
	AuthToken string
	topId     uint
	ls        net.Listener
	clients   *Client
	handlers  map[EventId]EventHandler
}

func NewServer() *Server {
	s := &Server{handlers: make(map[EventId]EventHandler)}
	registerEventHandlers(s)
	return s
}

func (s *Server) NewClient(conn net.Conn) *Client {
	c := new(Client)
	c.Subscriptions = NewSubscriptionList()
	c.Server = s
	c.Conn = conn
	// TODO: move this next line after mutex.lock
	c.Next = s.clients
	c.OutputChan = make(chan *Event, 16)
	c.TerminationChan = make(chan bool)
	c.Connected = true

	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	c.Id = s.topId
	s.topId++
	s.clients = c
	return c
}

func (s *Server) DeleteClient(c *Client) bool {
	c.Connected = false
	c.Conn.Close()
	//close(c.OutputChan)

	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	ptr := &s.clients
	for *ptr != nil {
		if *ptr == c {
			*ptr = c.Next
			clog.DebugXX("Deleting client %s, closing channel and socket", c)
			return true
		}
		ptr = &((*ptr).Next)
	}
	return false
}

func (s *Server) Broadcast(eid EventId, value interface{}, exclude_client *Client) {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	for c := s.clients; c != nil; c = c.Next {
		if c.Subscriptions.Includes(eid) && c != exclude_client {
			c.Put(eid, value)
		}
	}
}

func (s *Server) RegisterHandler(eid EventId, fn EventHandler) {
	if s.handlers[eid] != nil {
		clog.Warning("Replacing existing event handler for event %d", eid)
	}
	s.handlers[eid] = fn
}

func dumpValue(value []byte) string {
	if len(value) > 64 {
		return fmt.Sprintf("%q (%d additional bytes hidden)", value[:64], len(value)-64)
	}
	return fmt.Sprintf("%q", value)
}

func (s *Server) runClient(c *Client) {
	go func() {
		for {
			select {
			case event := <-c.OutputChan:
				if err := EncodeToStream(c.Conn, event.Id, event.Value); err != nil {
					clog.Warning("Client %s: %s", c, err)
					c.TerminationChan <- true
				}
			case <-c.TerminationChan:
				s.DeleteClient(c)
				return
			}
		}
	}()

	for {
		eid, value, err := c.Get()
		if err != nil {
			if err == io.EOF {
				clog.Info("Client %s closed connection", c)
			} else {
				clog.Warning("Client %s: %s", c, err)
			}
			break
		}

		clog.DebugX("Processing event %s(%d) from client %s, with %d bytes of payload", eid, eid, c, len(value))

		handler := s.handlers[eid]
		if handler != nil {
			if !c.Authenticated && eid != ClientAuthEvent && eid != ClientHelloEvent {
				c.Put(ServerAckEvent, SERVER_ACK_UNAUTHORIZED)
				break
			}
			if err = handler(c, eid, value); err != nil {
				clog.Error("Handler for event %s(%d) failed: %s, value=%s", eid, eid, err, dumpValue(value))
				break
			}
		} else {
			clog.Warning("No handler found for event id %d", eid)
			break
		}
	}
	c.TerminationChan <- true
}

func (s *Server) ListenAndServe(addr string, auth_token string) error {
	ls, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	clog.Info("Listening for clients at %s", ls.Addr())
	s.ls = ls

	s.AuthToken = auth_token

	go func() {
		for {
			conn, err := s.ls.Accept()
			if err != nil {
				clog.Error("Server could not accept connection: %s", err)
			} else {
				client := s.NewClient(conn)
				clog.Debug("Created new client %s", client)
				go s.runClient(client)
			}
		}
	}()
	return nil
}
