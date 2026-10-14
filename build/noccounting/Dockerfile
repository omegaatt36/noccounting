FROM gcr.io/distroless/static:nonroot
ARG TARGETARCH
COPY noccounting-${TARGETARCH} /noccounting
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 CMD ["/noccounting", "healthcheck"]
ENTRYPOINT ["/noccounting"]
