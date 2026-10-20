FROM m.daocloud.io/docker.io/library/golang:1.25-alpine AS build

WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gofly .

FROM m.daocloud.io/docker.io/library/alpine:3.22

RUN sed -i 's#dl-cdn.alpinelinux.org#mirrors.aliyun.com#g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/gofly ./gofly
COPY --from=build /src/static ./static
COPY --from=build /src/config/GeoLite2-City.mmdb ./config/GeoLite2-City.mmdb
COPY --from=build /src/import.sql ./import.sql
RUN mkdir -p /app/static/upload /data

EXPOSE 8081
ENTRYPOINT ["/app/gofly"]
