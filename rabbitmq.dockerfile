# FROM rabbitmq:3.7.14-management
FROM rabbitmq:3.7.14-management
# Install basics
RUN apt-get update \
 && apt-get install -y vim git zip wget

# ENV ADMIN_USER wataru
# ENV ADMIN_PASSWORD wataru
# ENV RABBITMQ_USER rabbitmq
# ENV RABBITMQ_PASSWORD rabbitmq
# ENV RABBITMQ_VHOST /third-project
# RUN rabbitmqctl add_vhost /third-project
# RUN rabbitmqctl add_user rabbitmq rabbitmq
# RUN rabbitmqctl set_permissions -p /third-project rabbitmq ".*" ".*" ".*"

ADD ./ca-cert.pem /etc/ssl/
# ADD ./server-key.pem /etc/ssl/
# ADD ./server-cert.pem /etc/ssl/ 

ADD ./init-rabbitmq.sh /
RUN chmod 755 ./init-rabbitmq.sh
ADD ./rabbitmq.config /etc/rabbitmq/
CMD ["/init-rabbitmq.sh"]
# ENTRYPOINT ["tail", "-f", "/dev/null"]