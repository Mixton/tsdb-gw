default:
	$(MAKE) all
test:
	go test -race ./...
check:
	$(MAKE) test
all:
	./scripts/build.sh

qa: build qa-common

qa-common:
	scripts/qa/gofmt.sh
	scripts/qa/ineffassign.sh
	scripts/qa/misspell.sh
	scripts/qa/gitignore.sh
	scripts/qa/unused.sh
	scripts/qa/vendor.sh
	scripts/qa/vet-high-confidence.sh
