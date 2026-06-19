//go:build integration

package faketelegram

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
			"POST /botTEST_TOKEN/sendMessage": defaultSendMessageResponse(),
		},
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	return fake
}

func (s *Server) URL() string {
	return s.server.URL
}

func (s *Server) BotAPIURL(token string) string {
	return s.server.URL + "/bot" + token
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

	if !ok && request.Method == http.MethodPost &&
		strings.HasPrefix(request.URL.Path, "/bot") &&
		strings.HasSuffix(request.URL.Path, "/sendMessage") {
		response = defaultSendMessageResponse()
		ok = true
	}
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

func defaultSendMessageResponse() Response {
	return Response{
		Status: http.StatusOK,
		Header: jsonHeader(),
		Body:   []byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":12345,"type":"private"},"text":"ok"}}`),
	}
}

func jsonHeader() http.Header {
	return http.Header{
		"Content-Type": []string{"application/json"},
	}
}
