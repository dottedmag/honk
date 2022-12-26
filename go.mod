module github.com/benjojo/honk-benjojo

go 1.19

require (
	github.com/andybalholm/cascadia v1.3.1
	github.com/gorilla/mux v1.8.0
	github.com/mattn/go-runewidth v0.0.13
	github.com/prometheus/client_golang v1.14.0
	github.com/ridge/must/v2 v2.0.0
	github.com/ridge/tj v0.3.0
	golang.org/x/crypto v0.0.0-20220411220226-7b82a4e95df4
	golang.org/x/image v0.0.0-20220413100746-70e8d0d3baa9
	golang.org/x/net v0.0.0-20220425223048-2871e0cb64e4
	humungus.tedunangst.com/r/go-sqlite3 v1.1.3
	humungus.tedunangst.com/r/webs v0.6.56
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/prometheus/client_model v0.3.0 // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	golang.org/x/sys v0.0.0-20220520151302-bc2c85ada10a // indirect
	google.golang.org/protobuf v1.28.1 // indirect
)

replace humungus.tedunangst.com/r/webs/image => ./image
