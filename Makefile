default:
	$(MAKE) all
test:
	bash -c "go test ./..."
check:
	$(MAKE) test
deps:
	bash -c "./scripts/depends.sh"
all:
	bash -c "./scripts/build.sh"

qa: build qa-common

qa-common:
	scripts/qa/gofmt.sh
	scripts/qa/ineffassign.sh
	scripts/qa/misspell.sh
	scripts/qa/gitignore.sh
	scripts/qa/unused.sh
	scripts/qa/vendor.sh
	scripts/qa/vet-high-confidence.sh