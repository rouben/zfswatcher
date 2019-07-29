FROM golang as build-env

ARG GOOS=linux
ARG GOARCH=amd64
ARG CGO_ENABLED=0

WORKDIR /go/src/app
COPY src ./
RUN \
    go get -d -v ./... && \
    go build -a -o /bin/zfswatcher

FROM ubuntu:bionic

RUN apt update && apt install -y zfs-dkms zfsutils-linux
COPY --from=build-env /bin/zfswatcher /
COPY etc/zfswatcher-docker.conf /config/zfswatcher.conf
COPY www/ /www

ENTRYPOINT [ "/zfswatcher", "-c", "/config/zfswatcher.conf" ]
CMD [ "-d" ]