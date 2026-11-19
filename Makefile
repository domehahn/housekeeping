BINARY := scm-cleaner
CARGO  := cargo

.PHONY: build
build:
	$(CARGO) build

.PHONY: release
release:
	$(CARGO) build --release
	@mkdir -p bin
	cp target/release/$(BINARY) bin/$(BINARY)

.PHONY: test
test:
	$(CARGO) test

.PHONY: check
check: fmt-check clippy test

.PHONY: clippy
clippy:
	$(CARGO) clippy -- -D warnings

.PHONY: fmt
fmt:
	$(CARGO) fmt

.PHONY: fmt-check
fmt-check:
	$(CARGO) fmt -- --check

.PHONY: clean
clean:
	$(CARGO) clean
	rm -rf bin/ *.jsonl *-plan.json
