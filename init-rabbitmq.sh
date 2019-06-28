#!/bin/bash

# Create Rabbitmq user
( sleep 30 ; \
rabbitmqctl add_user $ADMIN_USER $ADMIN_PASSWORD ; \
rabbitmqctl set_user_tags $ADMIN_USER administrator
rabbitmqctl add_user $RABBITMQ_USER $RABBITMQ_PASSWORD ; \
rabbitmqctl add_vhost $RABBITMQ_VHOST ; \
rabbitmqctl set_permissions -p / $ADMIN_USER  ".*" ".*" ".*" ; \
rabbitmqctl set_permissions -p $RABBITMQ_VHOST $RABBITMQ_USER  ".*" ".*" ".*" ; \
) &  
rabbitmq-server

