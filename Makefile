.PHONY: e2e_down
e2e_down:
	docker compose -f scripts/docker-compose.yaml down
.PHONY: e2e_up
e2e_up:
	docker compose -f scripts/docker-compose.yaml up -d
