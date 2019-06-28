# FROM rabbitmq:3.7.14-management
FROM rabbitmq:3.7.14-management
# Install basics
RUN apt-get update \
 && apt-get install -y vim git zip wget

ENV TZ "Asia/Tokyo"

ADD ./ca-cert.pem /etc/ssl/
# ADD ./server-key.pem /etc/ssl/
# ADD ./server-cert.pem /etc/ssl/ 

ADD ./init-rabbitmq.sh /
RUN chmod 755 ./init-rabbitmq.sh
ADD ./rabbitmq.config /etc/rabbitmq/
CMD ["/init-rabbitmq.sh"]
# ENTRYPOINT ["tail", "-f", "/dev/null"]