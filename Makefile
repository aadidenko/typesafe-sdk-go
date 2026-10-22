.PHONY: fmt fmt-check lint

fmt:
	@sh scripts/quality.sh fmt

fmt-check:
	@sh scripts/quality.sh fmt-check

lint:
	@sh scripts/quality.sh lint
