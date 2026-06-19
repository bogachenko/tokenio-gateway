//go:build integration

package fakebilling

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

type Request struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

type Server struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []Request
	response Response
}

func New() *Server {
	fake := &Server{
		response: Response{
			Status: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: []byte(`{"status":"ok"}`),
		},
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	return fake
}

func (s *Server) URL() string {
	return s.server.URL
}

func (s *Server) Close() {
	s.server.Close()
}

func (s *Server) SetResponse(response Response) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.response = response
}

func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	copied := make([]Request, len(s.requests))
	copy(copied, s.requests)
	return copied
}

func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests = nil
}

func (s *Server) handle(writer http.ResponseWriter, request *http.Request) {
	body := make([]byte, 0)
	if request.Body != nil {
		defer request.Body.Close()
		var err error
		body, err = readAll(request)
		if err != nil {
			http.Error(writer, "read request body", http.StatusInternalServerError)
			return
		}
	}

	s.mu.Lock()
	s.requests = append(s.requests, Request{
		Method: request.Method,
		Path:   request.URL.RequestURI(),
		Header: request.Header.Clone(),
		Body:   append([]byte(nil), body...),
	})
	response := s.response
	s.mu.Unlock()

	for key, values := range response.Header {
		for _, value := range values {
			writer.Header().Add(key, value)
		}
	}
	if response.Status == 0 {
		response.Status = http.StatusOK
	}
	writer.WriteHeader(response.Status)
	_, _ = writer.Write(response.Body)
}

func readAll(request *http.Request) ([]byte, error) {
	var body struct {
		Raw json.RawMessage `json:"-"`
	}
	_ = body

	data := make([]byte, 0, request.ContentLength)
	buf := make([]byte, 4096)
	for {
		n, err := request.Body.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return data, nil
			}
			return nil, err
		}
	}
}
