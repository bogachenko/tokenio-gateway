run:
	go run ./cmd/gateway

fmt:
	gofmt -w .

test:
	go test ./...

git:
	git add .
	git commit -a -m "$m"
	git push -u origin main
