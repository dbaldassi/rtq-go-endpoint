#!/bin/python3

import matplotlib.pyplot as plt
import csv
import sys

x = []
psnr = []
r = []
g = []
b = []
m = []

if len(sys.argv) >= 2:
    fig_name = sys.argv[1]
else:
    fig_name = "ssim and psnr"

with open('ssim.csv', 'r') as csvfile:
    lines = csv.reader(csvfile, delimiter=',')

    last = 0
    for row in lines:
        x.append(int(row[0]))
        psnr.append(float(row[1]))
        r.append(float(row[2]))
        g.append(float(row[3]))
        b.append(float(row[4]))
        m.append((r[-1] + g[-1] + b[-1])/3)
            
    last = x[-1]
    x = [float(i) / float(last) * 120.0 for i in x]
        
plt.plot(x, psnr, color = 'k', label = "psnr")
plt.plot(x, r, color = 'r', label = "red")
plt.plot(x, g, color = 'g', label = "green")
plt.plot(x, b, color = 'b', label = "blue")
plt.plot(x, m, color = 'y', label = "mean")

plt.title(fig_name, fontsize = 20)
plt.grid()
plt.legend()

with open('ssmim_psnr_mean.csv', 'a') as result:
    m_ssim = str(sum(m) / len(m))
    m_psnr = str(sum(psnr) / len(psnr))
    result.write(fig_name + "," + m_psnr + "," + m_ssim + "\n")

if len(sys.argv) == 3 and sys.argv[2] == 'save':
    plt.savefig(fig_name + '.png')
else:
    plt.show()
