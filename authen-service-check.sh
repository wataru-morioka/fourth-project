#!/bin/sh

ping go-authen-cluster -c 1 >> /dev/null

if [ $? -eq 0 ]; then
  ping go-socket-cluster -c 1 >> /dev/null
  
  if [ $? -eq 0 ]; then
    exit 0
  fi
fi

exit 1
