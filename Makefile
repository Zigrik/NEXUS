.PHONY: build run clean test install docker-build docker-run

build:
	go build -o bin/nexusd cmd/nexusd/main.go

run: build
	./bin/nexusd config.yaml

clean:
	rm -rf bin/
	go clean

test:
	go test -v ./...

install:
	go install ./cmd/nexusd

docker-build:
	docker build -t nexus:latest .

docker-run:
	docker run -d --name nexus \
		-p 8443:8443 \
		-p 9000:9000 \
		-v $(PWD)/config.yaml:/app/config.yaml:ro \
		-v $(PWD)/web:/app/web:ro \
		-v /etc/letsencrypt:/etc/letsencrypt:ro \
		nexus:latest