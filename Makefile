
all: honk

honk: schema.sql *.go go.mod
	go build -o honk

clean:
	rm -f honk

test:
	go test
