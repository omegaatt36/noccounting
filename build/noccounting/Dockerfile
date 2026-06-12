FROM gcr.io/distroless/static:nonroot
ARG TARGETARCH
COPY noccounting-${TARGETARCH} /noccounting
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/noccounting"]
