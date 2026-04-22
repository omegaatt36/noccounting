FROM gcr.io/distroless/static:nonroot
ARG TARGETARCH
COPY noccounting-webapp-${TARGETARCH} /webapp
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/webapp"]
