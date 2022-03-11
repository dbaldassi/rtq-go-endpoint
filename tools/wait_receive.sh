#!/bin/bash

# while [ 1 ]
# do
#     ./receiver.sh $ARGS &
# done

while [ 1 ]
do
    ARGS=$(socat tcp-l:8484 -)
    if [ "$ARGS" == "exit" ]
    then
	exit 0
    fi

    echo $ARGS
    
    ./receiver.sh $ARGS
done
