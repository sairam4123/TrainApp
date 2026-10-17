WORLD ?= tpjkkdi-sl ## Select the world to load

build-trainapp: ## Build the trainapp
	go build -o ./dist/trainapp ./cmd/trainapp

build-trainviz: ## Build the viz
	go build -o ./dist/trainviz ./cmd/trainviz

run: ## Run the project
	go run ./cmd/trainapp -record logs/run.ndjson -world $(WORLD)
	go run ./cmd/trainviz replay logs/run.ndjson

build-trainbench: ## Build the benchmark runner
	go build -o ./dist/trainbench ./cmd/trainbench

bench: ## Run every scenario under every rule and print a KPI table
	go run ./cmd/trainbench

build-traindemo: build-trainapp build-trainviz ## Build the scenario demo, with the trainapp and trainviz it runs
	go build -o ./dist/traindemo ./cmd/traindemo

demo: build-traindemo ## Pick a scenario, run it and replay it
	./dist/traindemo
