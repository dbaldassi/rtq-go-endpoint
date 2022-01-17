# rtq-go-endpoint

Added a set of script to run mendelbart experiments on congestion control for RTP over QUIC.

#### send.sh

This script is used to launch the docker container sending the video.

It parses the statistics from the output and write a csv file with the target bitrate and the transmitted bytes.

It shows a visual graph of this csv file and saves it.

Then, it computes the ssim of the received video.

This script takes 3 arguments :

* the transport : quic or udp
* the congestion control : naive, scream, scream-infer
* the latency

#### receive.sh

This script launch the receiver docker container.

This script takes 2 arguments (the same first two of the sender) :

* the transport
* the congestion control

#### link.sh

Script to control the link capacity using tc.

It is launched by ``send.sh`` as a detach process.

#### show_csv.py

This python script read csv file and show the corresponding graph.

#### ssim

ssim implement uses opencv.

To build :

```

g++ ssim.cpp -o ssim -lopencv_core -lopencv_imgproc -lopencv_videoio -lopencv_highgui -I/usr/include/opencv4

```

#### run.sh

This script runs the above set of scripts for every possible parameters (lantency, transport, congestion control) and generates bitrate graph, compute ssim for every experimentation.

Able to run on the loopback or two different machine. You must edit the run.sh and comment the receiver.sh and uncomment the socat command if you want to run the experiment on two different machine. Also don't forget to change the network settings in the link.sh.

#### wait_receive.sh

This script is used when running the experimentation on two different computer.

It waits a signal and parameters from the sender with socat.
