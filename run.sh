#!/bin/bash

LATENCIES=(1 50 150 300)
TRANSPORT=("quic" "udp")
CC=("naive" "scream" "scream-infer")

# LATENCIES=(300)
# TRANSPORT=("quic")
# CC=("naive")

for l in ${LATENCIES[@]}
do
    for t in ${TRANSPORT[@]}
    do
	for c in ${CC[@]}
	do
	    echo "$t $c $l"
	    ./receiver.sh $t $c > /dev/null &
	    # echo "$t $c $l" | socat tcp:192.168.1.33:8484 -
	    ./send.sh $t $c $l > /dev/null 
	    docker stop receiver sender
	    # sleep 10
	done
    done
done  
