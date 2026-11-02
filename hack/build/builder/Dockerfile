ARG GIMME_GO_VERSION=1.26.8
ARG HOST_ARCH=amd64

# Go tools are cross-compiled on the native build platform: running the Go
# toolchain under QEMU user emulation intermittently hangs foreign-arch builds.
# HOST_ARCH and ARCH are passed by build.sh as Go arch names.
FROM --platform=linux/${HOST_ARCH} quay.io/centos/centos:stream9 AS gotools
ARG GIMME_GO_VERSION
ARG ARCH

ENV GIMME_GO_VERSION=${GIMME_GO_VERSION} GOPATH="/go" GO111MODULE="on"

# reference to master is for an external repo and can't yet be changed
RUN mkdir -p /gimme && curl -sL \
    https://raw.githubusercontent.com/travis-ci/gimme/master/gimme | \
    HOME=/gimme bash >> /etc/profile.d/gimme.sh

# Install persistent go packages; cross-compiled binaries land in bin/linux_<arch>
RUN \
	test -n "${ARCH}" && \
	source /etc/profile.d/gimme.sh && \
	export GOOS=linux GOARCH="${ARCH}" CGO_ENABLED=0 && \
	go install mvdan.cc/sh/cmd/shfmt@latest && \
	go install github.com/mattn/goveralls@latest && \
	go install golang.org/x/lint/golint@latest && \
    go install -v github.com/golang/protobuf/protoc-gen-go@1643683 && \
	go install github.com/rmohr/go-swagger-utils/swagger-doc@latest && \
	go install -v sigs.k8s.io/controller-tools/cmd/controller-gen@v0.20.0 && \
	go install github.com/securego/gosec/v2/cmd/gosec@latest && \
	go install -v mvdan.cc/gofumpt@v0.6.0 && \
	if [ -d "${GOPATH}/bin/linux_${ARCH}" ]; then \
		mv "${GOPATH}/bin/linux_${ARCH}"/* "${GOPATH}/bin/" && \
		rmdir "${GOPATH}/bin/linux_${ARCH}"; \
	fi && \
	go clean -cache -modcache

FROM quay.io/centos/centos:stream9
ARG GIMME_GO_VERSION
LABEL maintainer="The KubeVirt Project <kubevirt-dev@googlegroups.com>"

RUN 	dnf -y install dnf-plugins-core && \
	dnf config-manager --set-enable crb && dnf update -y && \
	dnf install -y \
	cpio \
	diffutils \
	git \
	python3-pip \
	python3-devel \
	gcc \
	gcc-c++ \
	glibc-devel \
	findutils \
	autoconf \
	automake \
	libtool \
	jq \
	rsync-daemon \
	rsync \
	patch \
	libnbd-devel \
	nbdkit-devel \
	unzip \
    protobuf-compiler \
	&& dnf clean all



ENV GIMME_GO_VERSION=${GIMME_GO_VERSION} GOPATH="/go" GO111MODULE="on" GOLANGCI_LINT_VERSION=v1.60.1

# reference to master is for an external repo and can't yet be changed
RUN mkdir -p /gimme && curl -sL \
    https://raw.githubusercontent.com/travis-ci/gimme/master/gimme | \
    HOME=/gimme bash >> /etc/profile.d/gimme.sh

COPY --from=gotools /go/bin/ /go/bin/

RUN set -x && \
    source /etc/profile.d/gimme.sh && \
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$(go env GOROOT)"/bin $GOLANGCI_LINT_VERSION

# Until we use a version including the fix for this Bazel issue:
# https://github.com/bazelbuild/bazel/issues/11554
RUN ln -s /usr/bin/python3 /usr/bin/python

ADD entrypoint.sh /entrypoint.sh

ADD entrypoint-bazel.sh /entrypoint-bazel.sh

ADD rsyncd.conf /etc/rsyncd.conf

ENTRYPOINT ["/entrypoint.sh"]
