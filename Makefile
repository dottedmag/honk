
all: honk

honk: .preflightcheck schema.sql *.go go.mod
	go build -o honk

.preflightcheck: preflight.sh
	@sh ./preflight.sh

clean:
	rm -f honk

test:
	go test
