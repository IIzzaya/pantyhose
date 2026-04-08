.PHONY: build test test-docker clean

build:
	go build -o pantyhose-server.exe ./cmd/pantyhose-server
	go build -o pantyhose-client.exe ./cmd/pantyhose-client

test:
	go test -v -count=1 -timeout 60s ./...

test-docker:
	docker compose -f docker-compose.test.yml up --build --abort-on-container-exit

clean:
	rm -f pantyhose-server.exe pantyhose-client.exe
	rm -f pantyhose-server pantyhose-client
