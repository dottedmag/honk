all: honk

honk: *.go go.mod
	go build -o honk

clean:
	rm -f honk

test:
	go test
