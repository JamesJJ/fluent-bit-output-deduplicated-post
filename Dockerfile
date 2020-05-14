ARG BASE_IMAGE=golang:1.14

FROM $BASE_IMAGE as gocode

WORKDIR /root

ENV GOOS=linux\
    GOARCH=amd64

COPY /src/* /root/

RUN go get -d -v ./...

FROM gocode as gobuilder

RUN go build \
    -buildmode=c-shared \
    -o /out_http_post.so \
    ./

FROM fluent/fluent-bit:1.4.4 as fluentbit

COPY --from=gobuilder /out_http_post.so /fluent-bit/bin/

COPY ./etc /fluent-bit/etc

WORKDIR /fluent-bit/etc

EXPOSE 2020

CMD ["/fluent-bit/bin/fluent-bit", "--config", "/fluent-bit/etc/fluent-bit.conf"]
