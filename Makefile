# bvoice build. Requires make, the Xcode Command Line Tools (clang) and Go.
#
#   make build MODEL=small.en-q5_1  one binary with that model embedded -> dist/bvoice-<model>
#   make all                        build every model in MODELS
#   make test                       run the Go tests
#   make dist VERSION=v0.1.0        clean dist/, build all and write dist/SHA256SUMS
#   make clean                      remove build outputs (keeps downloaded models and whisper.cpp)
#
# Releases are made by GitHub Actions (.github/workflows/release.yml), not from a local machine.

SHELL := /bin/bash

WHISPER_REPO    := https://github.com/ggml-org/whisper.cpp
WHISPER_COMMIT  := 482279b8a18855ba741d7b88c30fcb3874691f59
WHISPER_VERSION := 1.9.5-dev
GGML_VERSION    := 0.28.0
WHISPER_DIR     := third_party/whisper.cpp
GGML            := $(WHISPER_DIR)/ggml/src
OBJ             := build/whisper
WHISPER_LIB     := $(OBJ)/libwhisper.a

MODEL      ?= small.en-q5_1
MODELS     ?= small.en-q5_1 large-v3-turbo-q5_0
MODEL_URL   = https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-$(MODEL).bin
MODEL_FILE  = build/models/ggml-$(MODEL).bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)
NPROC   := $(shell sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 4)

LDFLAGS := -s -w -X main.version=$(VERSION) -X main.modelName=$(MODEL)

.PHONY: all deps lib model build test test-scripts vet lint fmt dist clean

all:
	@for m in $(MODELS); do $(MAKE) --no-print-directory build MODEL=$$m || exit 1; done

deps: lib go.sum

# Resolve and verify Go modules (writes go.sum on the first run; commit it).
go.sum: go.mod
	go mod tidy
	@touch go.sum

# whisper.cpp is fetched at a pinned commit and compiled into one static library, with the Metal
# shaders embedded in it.

$(WHISPER_DIR)/include/whisper.h:
	mkdir -p $(WHISPER_DIR)
	git -C $(WHISPER_DIR) init -q
	git -C $(WHISPER_DIR) fetch -q --depth 1 $(WHISPER_REPO) $(WHISPER_COMMIT)
	git -C $(WHISPER_DIR) checkout -q FETCH_HEAD

lib: $(WHISPER_DIR)/include/whisper.h
	@$(MAKE) --no-print-directory -j$(NPROC) $(WHISPER_LIB)

ifeq ($(origin CXX),default)
CXX := c++
endif

W_DEFS     := -DNDEBUG -DGGML_SCHED_MAX_COPIES=4 -D_XOPEN_SOURCE=600
W_USE      := -DGGML_USE_CPU
W_INC      := -I$(WHISPER_DIR)/ggml/include -I$(GGML) -I$(GGML)/ggml-cpu -I$(WHISPER_DIR)/include -I$(WHISPER_DIR)/src -I$(OBJ)
W_CFLAGS   := -O3 -fPIC -w -std=gnu11
W_CXXFLAGS := -O3 -fPIC -w -std=gnu++17

BASE_SRC := $(addprefix ggml/src/,ggml.c ggml-alloc.c ggml-quants.c ggml.cpp ggml-backend.cpp ggml-opt.cpp \
	ggml-threading.cpp gguf.cpp ggml-backend-meta.cpp)
REG_SRC  := ggml/src/ggml-backend-reg.cpp ggml/src/ggml-backend-dl.cpp
CPU_SRC  := $(addprefix ggml/src/ggml-cpu/,ggml-cpu.c ggml-cpu.cpp repack.cpp hbm.cpp quants.c traits.cpp \
	amx/amx.cpp amx/mmq.cpp tiled/tiled.cpp tiled/tiled-kernel.cpp binary-ops.cpp unary-ops.cpp vec.cpp ops.cpp)
WHI_SRC  := src/whisper.cpp src/aneforge/whisper-aneforge.cpp
CPU_DEFS := -DGGML_USE_CPU_REPACK

ifneq ($(filter arm64 aarch64,$(UNAME_M)),)
CPU_SRC  += ggml/src/ggml-cpu/arch/arm/quants.c ggml/src/ggml-cpu/arch/arm/repack.cpp
CPU_ARCH := $(shell $(CC) -mcpu=native -x c -c /dev/null -o /dev/null 2>/dev/null && echo -mcpu=native)
else
CPU_SRC  += ggml/src/ggml-cpu/arch/x86/quants.c ggml/src/ggml-cpu/arch/x86/repack.cpp
CPU_ARCH := -march=native
endif

ifeq ($(UNAME_S),Darwin)
W_DEFS   += -D_DARWIN_C_SOURCE
W_USE    += -DGGML_USE_METAL -DGGML_USE_BLAS
ACCEL    := -DACCELERATE_NEW_LAPACK -DACCELERATE_LAPACK_ILP64
CPU_DEFS += -DGGML_USE_ACCELERATE $(ACCEL)
ifeq ($(CPU_ARCH),)
CPU_ARCH := -mcpu=apple-m1
endif
METAL_SRC := $(addprefix ggml/src/ggml-metal/,ggml-metal.cpp ggml-metal-device.m ggml-metal-device.cpp \
	ggml-metal-common.cpp ggml-metal-context.m ggml-metal-fusion.cpp ggml-metal-ops.cpp ggml-metal-tuning.cpp)
BLAS_SRC  := ggml/src/ggml-blas/ggml-blas.cpp
# every kernel except the two that are only included by others
METAL_KERNELS := $(filter-out fa_common fa_vec_common,$(basename $(notdir $(wildcard $(GGML)/ggml-metal/kernels/*.metal))))
METAL_EMBED   := $(addprefix $(OBJ)/metal/,$(addsuffix .s.o,$(METAL_KERNELS)))
else
W_DEFS += -D_GNU_SOURCE
endif

objs = $(addprefix $(OBJ)/,$(addsuffix .o,$(1)))
BASE_OBJ  := $(call objs,$(BASE_SRC))
REG_OBJ   := $(call objs,$(REG_SRC))
CPU_OBJ   := $(call objs,$(CPU_SRC))
WHI_OBJ   := $(call objs,$(WHI_SRC))
METAL_OBJ := $(call objs,$(METAL_SRC))
BLAS_OBJ  := $(call objs,$(BLAS_SRC))
ALL_OBJ   := $(BASE_OBJ) $(REG_OBJ) $(CPU_OBJ) $(WHI_OBJ) $(METAL_OBJ) $(BLAS_OBJ) $(METAL_EMBED)

# per-group compile flags
$(REG_OBJ):   EXTRA := $(W_USE)
$(CPU_OBJ):   EXTRA := $(CPU_DEFS) $(CPU_ARCH)
$(WHI_OBJ):   EXTRA := $(W_USE) -DWHISPER_VERSION=\"$(WHISPER_VERSION)\"
$(METAL_OBJ): EXTRA := -DGGML_METAL_EMBED_LIBRARY
$(BLAS_OBJ):  EXTRA := -DGGML_BLAS_USE_ACCELERATE $(ACCEL)

$(WHISPER_LIB): $(ALL_OBJ)
	rm -f $@
	ar rcs $@ $^
	@echo "built $@"

$(OBJ)/ggml-version.h:
	@mkdir -p $(dir $@)
	printf '#pragma once\n#define GGML_VERSION "%s"\n#define GGML_COMMIT "%s"\n' $(GGML_VERSION) $(shell echo $(WHISPER_COMMIT) | cut -c1-7) > $@

$(OBJ)/%.c.o: $(WHISPER_DIR)/%.c $(OBJ)/ggml-version.h
	@mkdir -p $(dir $@)
	$(CC) $(W_CFLAGS) $(W_DEFS) $(EXTRA) $(W_INC) -c $< -o $@

$(OBJ)/%.cpp.o: $(WHISPER_DIR)/%.cpp $(OBJ)/ggml-version.h
	@mkdir -p $(dir $@)
	$(CXX) $(W_CXXFLAGS) $(W_DEFS) $(EXTRA) $(W_INC) -c $< -o $@

$(OBJ)/%.m.o: $(WHISPER_DIR)/%.m $(OBJ)/ggml-version.h
	@mkdir -p $(dir $@)
	$(CC) -O3 -fPIC -w $(W_DEFS) $(EXTRA) $(W_INC) -c $< -o $@

# Metal shaders embedded in the binary: each kernel is flattened with its headers and wrapped in an
# assembly file that .incbin's it.
$(OBJ)/metal/%.s: $(GGML)/ggml-metal/kernels/%.metal $(GGML)/ggml-common.h $(GGML)/ggml-metal/ggml-metal-impl.h
	@mkdir -p $(dir $@)
	@K=$(GGML)/ggml-metal/kernels; H="$$K/common.h"; \
	if grep -q '#include "dequantize.h"' $<; then H="$$H $$K/dequantize.h"; fi; \
	if grep -q '#include "quantize.h"' $<; then H="$$H $$K/quantize.h"; fi; \
	if grep -q '#include "fa_common.metal"' $<; then H="$$H $$K/fa_common.metal"; fi; \
	if grep -q '#include "fa_vec_common.metal"' $<; then H="$$H $$K/fa_vec_common.metal"; fi; \
	cat $$H $< \
	| sed -e '/#include "common.h"/d' -e '/#include "dequantize.h"/d' -e '/#include "quantize.h"/d' \
	      -e '/#include "fa_common.metal"/d' -e '/#include "fa_vec_common.metal"/d' -e '/#pragma once/d' \
	| sed -e '/__embed_ggml-common.h__/r $(GGML)/ggml-common.h' -e '/__embed_ggml-common.h__/d' \
	| sed -e '/#include "ggml-metal-impl.h"/r $(GGML)/ggml-metal/ggml-metal-impl.h' -e '/#include "ggml-metal-impl.h"/d' \
	> $(OBJ)/metal/$*.metal
	@printf '.section __DATA,__ggml_metallib\n.globl _ggml_metallib_%s_start\n_ggml_metallib_%s_start:\n.incbin "%s"\n.globl _ggml_metallib_%s_end\n_ggml_metallib_%s_end:\n' \
		$* $* $(abspath $(OBJ)/metal/$*.metal) $* $* > $@

$(OBJ)/metal/%.s.o: $(OBJ)/metal/%.s
	$(CC) -c $< -o $@

model: $(MODEL_FILE)

$(MODEL_FILE):
	mkdir -p build/models
	curl -L --fail --retry 5 --retry-delay 10 --retry-all-errors --progress-bar -o $@.part $(MODEL_URL)
	mv $@.part $@

build: deps model
	cp $(MODEL_FILE) internal/model/model.bin
	mkdir -p dist
	CGO_ENABLED=1 go build -trimpath -tags bvoice_model -ldflags "$(LDFLAGS)" -o dist/bvoice-$(MODEL) ./cmd/bvoice
	@echo "built dist/bvoice-$(MODEL) ($$(du -h dist/bvoice-$(MODEL) | cut -f1))"

# The real-transcription test uses the first downloaded model (run `make model` first to include it).
test: deps
	BVOICE_TEST_MODEL="$$(ls $(CURDIR)/build/models/*.bin 2>/dev/null | head -1)" CGO_ENABLED=1 go test ./...

# Tests for the release scripts in scripts/.
test-scripts:
	bash scripts/next-version_test.sh

vet: deps
	CGO_ENABLED=1 go vet ./...

# Linters and formatters are configured in .golangci.yml (golangci-lint v2).
lint: deps
	CGO_ENABLED=1 golangci-lint run ./...

fmt:
	golangci-lint fmt ./...

# Release files: every binary in MODELS plus SHA256SUMS. GitHub Actions also passes
# CPU_ARCH=-mcpu=apple-m1 so the binaries run on every Apple silicon Mac, not just the build machine.
dist:
	rm -rf dist
	$(MAKE) --no-print-directory all
	cd dist && shasum -a 256 bvoice-* > SHA256SUMS
	@cat dist/SHA256SUMS

clean:
	rm -rf dist $(OBJ) internal/model/model.bin
