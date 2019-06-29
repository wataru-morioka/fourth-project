FROM nginx:latest

RUN useradd -m -s /bin/bash -u 1000 wataru

RUN sed -i 's/user\ \ nginx\;/user\ \ wataru\;/g' /etc/nginx/nginx.conf

RUN apt-get update && apt-get install -y vim
RUN apt-get install -y curl
RUN apt-get install -y procps

RUN /bin/cp -f /usr/share/zoneinfo/Asia/Tokyo /etc/localtime

RUN echo 'stream {\n\
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
}' >> /etc/nginx/nginx.conf

WORKDIR /var/www
