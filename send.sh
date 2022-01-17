#!/bin/bash

# $1 : transport protocol -> quic | udp
# $2 : congestion control -> naive | scream | scream-infer
# $3 : latency in ms

# set link limit with tx
./link.sh 1 0.5 60 30 $3 &

docker run --name sender --rm -v $HOME/Videos/:/root -e VIDEOS="/root/1293kbs.mp4" -e SENDER_PARAMS="-transport $1 -cc $2" -e ROLE=sender -e RECEIVER=127.0.0.1:4242 --network host rtp-go-endpoint | tee out.log

# delete qdisc
./link.sh 

cat out.log | grep bitrate | tr -d ',' | awk '{print $3","$4","$11}' > bitrate.csv

sed -i s/",$"//g bitrate.csv

./show_csv.py "bitrate_$1-$2-$3" save

./ssim ~/Videos/1293kbs.mp4 ./out.y4m
./show_ssim.py "ssim_$1-$2-$3" save
rm -rf out.y4m

# in : RTP receiveTime(ms) PayloadType ssrc sequenceNumber timestamp marker?(bool) len(payload)
