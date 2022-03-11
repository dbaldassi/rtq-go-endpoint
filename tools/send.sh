#!/bin/bash

# $1 : transport protocol -> quic | udp
# $2 : congestion control -> naive | scream | scream-infer
# $3 : QUIC cc -> newreno | nocc
# $4 : latency in ms

# run docker
VIDEO=moi.mkv
OUT_DIR=out

mkdir -p $OUT_DIR

docker run --name sender --rm -v $PWD:/root -e VIDEOS="/root/videos/$VIDEO" -e SENDER_PARAMS="-transport $1 -cc $2" -e QLOGDIR="/root/log_dir" -e ROLE=sender -e RECEIVER=172.17.0.2:4242 -e TC_CONFIG="$4 $(cat link_limit.csv | tr '\n' ' ')" --cap-add=NET_ADMIN rtq-go-endpoint-$3 | tee $OUT_DIR/out.log

cd $OUT_DIR
rm -r link_limit.csv
ln -s ../link_limit.csv

## parse stats
cat out.log | grep bitrate | tr -d ',' | awk '{print $3","$4}' > bitrate.csv
cat out.log | grep SCReAM_stats | tr -d ',' | awk '{print $3","$4","$6","$7","$8","$9","$10","$11","$23","$24}' > scream.csv

sed -i s/",$"//g bitrate.csv
sed -i s/",$"//g scream.csv

../tools/show_csv.py "stats_$1-$2-$3-$4" save

## ssim
../tools/ssim ~/Vidéos/$VIDEO ./out.y4m
../tools/show_ssim.py "ssim_$1-$2-$3-$4" save

## rename csv files to save it
cp bitrate.csv "bitrate_$1-$2-$3-$4.csv"
cp scream.csv "scream_$1-$2-$3-$4.csv"

## remove video because it is quite big
rm -rf out.y4m

## rename qlog file
cd ../log_dir/

if [ "$1" == "quic" ]
then
    mv $(ls -1 -t | head -n 1) "../$OUT_DIR/qlog_$1-$2-$3-$4.qlog"
fi

# in : RTP receiveTime(ms) PayloadType ssrc sequenceNumber timestamp marker?(bool) len(payload)
