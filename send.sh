#!/bin/bash

# $1 : transport protocol -> quic | udp
# $2 : congestion control -> naive | scream | scream-infer
# $3 : latency in ms
# $4 : QUIC cc -> newreno | nocc

# run docker
VIDEO=moi.mkv

docker run --name sender --rm -v $HOME/Vidéos/:/root -e VIDEOS="/root/$VIDEO" -e SENDER_PARAMS="-transport $1 -cc $2" -e QLOGDIR="/root/log_dir" -e ROLE=sender -e RECEIVER=172.17.0.2:4242 -e TC_CONFIG="$4 $(cat link_limit.csv | tr '\n' ' ')" --cap-add=NET_ADMIN rtq-go-endpoint-$3 | tee out.log

## parse stats
cat out.log | grep bitrate | tr -d ',' | awk '{print $3","$4}' > bitrate.csv
cat out.log | grep QUIC_stats | tr -d ',' | awk '{print $3","$4","$5","$6","$7","$8","$9}' > qstats.csv
cat out.log | grep SCReAM_stats | tr -d ',' | awk '{print $3","$4","$6","$7","$8","$9","$10","$11","$23","$24}' > scream.csv

sed -i s/",$"//g bitrate.csv
sed -i s/",$"//g qstats.csv
sed -i s/",$"//g scream.csv

./show_csv.py "stats_$1-$2-$3-$4" save

## ssim
./ssim ~/Vidéos/$VIDEO ./out.y4m
./show_ssim.py "ssim_$1-$2-$3-$4" save
# rm -rf out.y4m

## rename qlog file
cd ~/Vidéos/log_dir/
# mv $(ls -1 -t | head -n 1) "ssim_$1-$2-$3-$4.qlog"

# in : RTP receiveTime(ms) PayloadType ssrc sequenceNumber timestamp marker?(bool) len(payload)
