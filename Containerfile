FROM golang:1.22

ARG entrypoint=/opt/redfi

COPY . /src
WORKDIR /src
RUN go build ./cmd/redfi
RUN mkdir -p /opt
RUN cp /src/redfi /opt
RUN printf '{"rules":[]}' > /etc/redfi.json
WORKDIR /opt
RUN rm -rf /src

ENTRYPOINT ["/opt/redfi", "-plan", "/etc/redfi.json"]

