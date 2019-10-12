#!/bin/sh

process=`ps ax | grep authen-server.go | grep -v grep | wc -l`
if [ $process -eq 0 ]; then
  exit 1
fi

exit 0
