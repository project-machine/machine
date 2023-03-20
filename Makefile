BINS := bin/machine bin/machined

.PHONY: all clean
all: $(BINS)

clean:
	rm -f -v $(BINS)

bin/machine: cmd/machine/*.go cmd/machine/cmd/*.go pkg/*/*.go
	go build -o $@ cmd/machine/*.go

bin/machined: cmd/machined/*.go cmd/machined/cmd/*.go pkg/*/*.go
	go build -o $@ cmd/machined/*.go
