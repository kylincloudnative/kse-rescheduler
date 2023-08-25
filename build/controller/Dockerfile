# Copyright 2023-KylinSoft Co.,Ltd.

# kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
# in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
# the scheduling-retries defined in annotations.


# Build
FROM golang:1.19.6 as build_context
ENV GO111MODULE "on"
ENV GOPROXY="https://goproxy.cn,direct"
WORKDIR /workspace
ADD . /workspace/
RUN make build-controller


FROM alpine:3.13

COPY --from=build_context /workspace/_build/kse-rescheduler  /

ENTRYPOINT ["/kse-rescheduler"]
