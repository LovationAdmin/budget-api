.PHONY: help build run test clean docker-up docker-down

APP_NAME=budget-api
POSTGRES_CONTAINER=budget-postgres

help:
	@echo "Available commands:"
	@echo "  make setup       - Setup complete (Docker + deps)"
	@echo "  make run         - Run the API"
	@echo "  make test-api    - Test the API"
	@echo "  make docker-up   - Start PostgreSQL"
	@echo "  make docker-down - Stop PostgreSQL"

install:
	go mod download
	go mod tidy

build:
	go build -o $(APP_NAME) main.go

run:
	go run main.go

test-api:
	chmod +x test.sh
	./test.sh

docker-up:
	docker run --name $(POSTGRES_CONTAINER) \
		-e POSTGRES_PASSWORD=mysecret \
		-e POSTGRES_DB=budget_db \
		-p 5432:5432 \
		-d postgres:15 || docker start $(POSTGRES_CONTAINER)

docker-down:
	docker stop $(POSTGRES_CONTAINER) || true
	docker rm $(POSTGRES_CONTAINER) || true

setup: docker-up install
	@echo "✅ Setup complete!"
	@echo "Create .env: cp .env.example .env"
	@echo "Then: make run"

clean:
	rm -f $(APP_NAME)