#!/bin/bash

LATENCIES=(50)
TRANSPORT=("quic")
CC=("naive" "scream")
AZD=("newreno")

# LATENCIES=(300)
# TRANSPORT=("quic")
# CC=("naive")

for l in ${LATENCIES[@]}
do
    for t in ${TRANSPORT[@]}
    do
	for c in ${CC[@]}
	do
	    for k in ${AZD[@]}
	    do
		echo "$t $c $l $k"
		./tools/receiver.sh $t $c $k $l > /dev/null &
		sleep 1
		# echo "$t $c $k" | socat tcp:192.168.1.47:8484 -
		./tools/send.sh $t $c $k $l > /dev/null 
		docker stop receiver sender 
		# sleep 10
	    done
	done
    done
done  
