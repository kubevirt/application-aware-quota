UNAME_ARCH     := $(shell uname -m)
ifeq ($(UNAME_ARCH),x86_64)
	TEMP_ARCH = amd64
else ifeq ($(UNAME_ARCH),aarch64)
	TEMP_ARCH = arm64
else
	TEMP_ARCH := $(UNAME_ARCH)
endif

GOARCH ?= $(TEMP_ARCH)
GOOS = linux

label_sidecar:
	echo "Building label_sidecar for ${GOOS}/${GOARCH}"
	go build -o label_sidecar ./cmd/
