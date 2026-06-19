//go:build integration

package fakeanthropic

import (
	"io"
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

	mu        sync.Mutex
	requests  []Request
	responses map[string]Response
}

func New() *Server {
	fake := &Server{
		responses: map[string]Response{
			"POST /v1/messages": {
				Status: http.StatusOK,
				Header: jsonHeader(),
				Body:   []byte(`{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`),
			},
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

func (s *Server) SetResponse(method, path string, response Response) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.responses[method+" "+path] = response
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
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, "read request body", http.StatusInternalServerError)
		return
	}
	defer request.Body.Close()

	s.mu.Lock()
	s.requests = append(s.requests, Request{
		Method: request.Method,
		Path:   request.URL.RequestURI(),
		Header: request.Header.Clone(),
		Body:   append([]byte(nil), body...),
	})
	response, ok := s.responses[request.Method+" "+request.URL.Path]
	s.mu.Unlock()

	if !ok {
		http.NotFound(writer, request)
		return
	}
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

func jsonHeader() http.Header {
	return http.Header{
		"Content-Type": []string{"application/json"},
	}
}
