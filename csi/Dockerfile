FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/clavis-csi .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/clavis-csi /usr/local/bin/clavis-csi
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/clavis-csi"]
