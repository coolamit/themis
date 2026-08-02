# Themis gate suite — `make` (or `make check`) must pass before every commit.
# Each gate is also an individual target: make fmt / vet / test / build /
# shellcheck / bats / actionlint / consistency.

.PHONY: check fmt vet test build shellcheck bats actionlint consistency

check: fmt vet test build shellcheck bats actionlint consistency

# gofmt -l exits 0 even when files need formatting; gate on its output.
fmt:
	@echo "gofmt -l ."
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		echo "needs gofmt:"; \
		echo "$$files"; \
		exit 1; \
	fi

vet:
	go vet ./...

test:
	go test -count=1 ./...

build:
	CGO_ENABLED=0 go build ./...

shellcheck:
	find scripts -name '*.sh' -print0 | xargs -0 shellcheck

bats:
	bats scripts/tests/

actionlint:
	actionlint .github/workflows/ci.yml .github/workflows/release.yml examples/themis.yml

consistency:
	./scripts/tests/check-action-consistency.sh action.yml
