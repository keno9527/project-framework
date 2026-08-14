.PHONY: check check-backend check-frontend

check: check-backend check-frontend

check-backend:
	cd backend && go test ./...

check-frontend:
	cd frontend && npm run check
