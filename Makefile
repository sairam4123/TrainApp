build-trainapp:
	go build -o ./dist/trainapp ./cmd/trainapp

build-trainviz:
	go build -o ./dist/trainviz ./cmd/trainviz

run-dl:
	go run ./cmd/trainapp -record logs/run1.ndjson -world tpjkkdi
	go run ./cmd/trainviz replay logs/run1.ndjson

run-sl:
	go run ./cmd/trainapp -record logs/run1.ndjson -world tpjkkdi-sl
	go run ./cmd/trainviz replay logs/run1.ndjson