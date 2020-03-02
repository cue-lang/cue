# target: cue-builder
ARG GOLANG_VERSION
FROM docker.io/golang:${GOLANG_VERSION}-alpine AS cue-builder
ARG CUE_VERSION
ENV \
	OUTDIR='/out' \
	GO111MODULE='on'
RUN set -eux && \
	apk add --no-cache \
		git
WORKDIR /go/src/cuelang.org/go
COPY go.mod /go/src/cuelang.org/go/
COPY go.sum /go/src/cuelang.org/go/
RUN set -eux && \
	go mod download
COPY . /go/src/cuelang.org/go/
RUN set -eux && \
	CGO_ENABLED=0 GOBIN=${OUTDIR}/usr/bin/ go install \
		-a -v \
		-tags='osusergo,netgo' \
		-installsuffix='netgo' \
		-ldflags="-s -w -X cuelang.org/go/cmd/cue/cmd.version=${CUE_VERSION} \"-extldflags=-static\"" \
	./cmd/cue

# target: cue
FROM gcr.io/distroless/static:latest AS cue
COPY --from=cue-builder /out/ /
ENTRYPOINT ["/usr/bin/cue"]
