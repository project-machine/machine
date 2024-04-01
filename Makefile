BINS := bin/machine bin/machined

.PHONY: all clean
all: $(BINS)

.PHONY: test
test: test-api

clean:
	rm -f -v $(BINS)

bin/machine: cmd/machine/cmd/*.go pkg/*/*.go
	go build -o $@ cmd/machine/cmd/*.go

bin/machined: cmd/machined/cmd/*.go pkg/*/*.go
	go build -o $@ cmd/machined/cmd/*.go

test-api:
	go test pkg/api/*.go
