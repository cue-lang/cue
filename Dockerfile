FROM gcr.io/distroless/static:latest
COPY cue /usr/bin/cue
ENTRYPOINT ["/usr/bin/cue"]
