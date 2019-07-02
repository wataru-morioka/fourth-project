# FROM nginx:1.17
FROM ubuntu:18.04
# Install basics
RUN apt-get update \
 && apt-get install -y iproute2 iputils-ping software-properties-common vim curl tzdata nginx \
 && ln -sf /usr/share/zoneinfo/Asia/Tokyo /etc/localtime \
 && DEBIAN_FRONTEND=noninteractive apt-get -y install tripwire -y \
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

ADD ./twcfg.txt /etc/tripwire/twcfg.txt 
ADD ./gmail /etc/postfix/gmail

RUN echo "Y" | twadmin -m G -S /etc/tripwire/site.key -Q wataru \
 && echo "Y" | twadmin -m G -L /etc/tripwire/local.key -P wataru \
 && twadmin -m F -c /etc/tripwire/tw.cfg -S /etc/tripwire/site.key -Q wataru /etc/tripwire/twcfg.txt \
 && twadmin -m P -c /etc/tripwire/tw.cfg -S /etc/tripwire/site.key -Q wataru /etc/tripwire/twpol.txt \
 && chown root /etc/postfix/gmail \
 && chmod 600 /etc/postfix/gmail \
 && postmap /etc/postfix/gmail 

ADD ./main.cf /etc/postfix/main.cf

# RUN /etc/init.d/postfix restart 