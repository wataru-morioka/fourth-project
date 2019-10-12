FROM golang:1.11.1
# Install basics
RUN apt-get update \
 && apt-get install -y vim git zip wget \
 && go get -u google.golang.org/grpc \
 && go get -u github.com/golang/protobuf/protoc-gen-go \
 && wget https://github.com/protocolbuffers/protobuf/releases/download/v3.6.1/protoc-3.6.1-linux-x86_64.zip \
 && unzip protoc-3.6.1-linux-x86_64.zip -d protoc \
 && cd protoc \
 && mv bin/protoc /usr/bin \
 && go get github.com/streadway/amqp \
 && go get github.com/go-redis/redis \
 && go get github.com/gomodule/redigo/redis \
 && go get github.com/jinzhu/gorm \
 && go get google.golang.org/api/option \
 && go get github.com/sirupsen/logrus \
 && go get gopkg.in/natefinch/lumberjack.v2 \
 && go get firebase.google.com/go \
 && go get firebase.google.com/go/messaging \
 && /bin/cp -f /usr/share/zoneinfo/Asia/Tokyo /etc/localtime

ENV PATH $PATH:$GOPATH/bin

WORKDIR /usr/local
RUN mkdir instantclient_19_3 
ADD ./instantclient_19_3/ /usr/local/instantclient_19_3/
ADD ./oci8.pc /usr/lib/pkgconfig/oci8.pc
ADD ./go-bash.profile /root/.bashrc
WORKDIR /go/src
ADD ./init.sh /go/src/init.sh 
RUN chmod 755 /go/src/init.sh 
ADD ./authen-process-check.sh /go/src/authen-process-check.sh 
RUN chmod 755 /go/src/authen-process-check.sh 
ADD ./socket-process-check.sh /go/src/socket-process-check.sh 
RUN chmod 755 /go/src/socket-process-check.sh 
ADD ./consumer-process-check.sh /go/src/consumer-process-check.sh 
RUN chmod 755 /go/src/consumer-process-check.sh 
ADD ./notifier-process-check.sh /go/src/notifier-process-check.sh 
RUN chmod 755 /go/src/notifier-process-check.sh 
#WORKDIR /root
#RUN . .bash_profile; exit 0
#WORKDIR /go/src/gRPC/instantclient_19_3
#RUN ln -s libclntsh.so.19.1 libclntsh.so; exit 0
RUN apt-get install libaio1 libaio-dev
WORKDIR /go/src/gRPC
# RUN go get github.com/mattn/go-oci8

#RUN mv /bin/sh /bin/sh_tmp && ln -s /bin/bash /bin/sh
#RUN source .bashrc
#RUN rm /bin/sh && mv /bin/sh_tmp /bin/sh

#WORKDIR /go/src/gRPC

# EXPOSE 8080

# ENTRYPOINT ["/bin/sh /go/src/init.sh"]