# FROM nginx:1.17
FROM ubuntu:18.04
# Install basics
RUN apt-get update \
 && apt-get install -y iproute2 iputils-ping software-properties-common vim curl tzdata nginx \
 && ln -sf /usr/share/zoneinfo/Asia/Tokyo /etc/localtime \
 && add-apt-repository ppa:oisf/suricata-stable \
 && apt-get update \
 && apt-get install -y suricata \
 && suricata-update \
 && useradd -m -s /bin/bash -u 1000 wataru \
 && sed -i 's/user\ \ nginx\;/user\ \ wataru\;/g' /etc/nginx/nginx.conf \
 && echo 'stream {\n\
    error_log /var/log/nginx/stream.log info;\n\
    upstream go-authen {\n\
        server go-authen-cluster:50030;\n\
    }\n\
    server { \n\
        listen 50030;\n\
        proxy_pass go-authen;\n\
    }\n\
    upstream go-socket {\n\
        server go-socket-cluster:50050;\n\
    }\n\ 
    server {\n\
        listen 50050;\n\
        proxy_pass go-socket;\n\ 
    }\n\ 
    upstream rabbitmq {\n\
        server rabbitmq-cluster:5671;\n\
    }\n\ 
    server {\n\
        listen 5671;\n\
        proxy_pass rabbitmq;\n\ 
    }\n\ 
}' >> /etc/nginx/nginx.conf 