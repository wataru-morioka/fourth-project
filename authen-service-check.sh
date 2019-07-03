#!/bin/sh

ping go-authen-cluster -c 1 >> /dev/null

if [ $? -eq 0 ]; then
  exit 0
fi

exit 1
