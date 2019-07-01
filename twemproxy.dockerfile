FROM jgoodall/twemproxy:latest
# Install basics
# RUN apt-get update \
#  && apt-get install -y vim git zip wget

RUN /bin/cp -f /usr/share/zoneinfo/Asia/Tokyo /etc/localtime

ADD ./supervisord.conf /etc/supervisor/supervisord.conf