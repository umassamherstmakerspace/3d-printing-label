server:
	docker buildx build --platform linux/amd64,linux/arm64 -t mkrcx/3d-printing-label-server -f src/server/Dockerfile . --push