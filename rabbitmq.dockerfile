# FROM rabbitmq:3.7.14-management
FROM rabbitmq:3.7.14-management
# Install basics
RUN apt-get update \
 && apt-get install -y vim git zip wget

ENV TZ "Asia/Tokyo"

ADD ./ca-cert.pem /etc/ssl/

ADD ./rabbitmq.config /etc/rabbitmq/
ADD ./.erlang.cookie /var/lib/rabbitmq/.erlang.cookie
RUN chmod 400 /var/lib/rabbitmq/.erlang.cookie