#!/bin/python3

import matplotlib.pyplot as plt
import csv
import sys

x = []
y = []
z = []

csv_file = sys.argv.pop(1)

if len(sys.argv) >= 2:
    fig_name = sys.argv[1]
else:
    fig_name = "result"

with open(csv_file, 'r') as csvfile:
    lines = csv.reader(csvfile, delimiter=',')

    last = 0
    for row in lines:
        row0 = float(row[0])
        if(last != row0):
            x.append(row0)
            y.append(int(row[1]))
            if(len(row) > 2):
                z.append(int(row[2]))
            last = row0
        else:
            y[-1] += int(row[1])
            if(len(row) > 2):
                z[-1] += int(row[2])
            
    m = x[0]
    x = [float(i - m)/float(1000) for i in x]
    # y = [i/1000 for i in y]
        
plt.plot(x, y, color = 'r', label = "target")

if(len(z) > 0):
    plt.plot(x, z, color = 'b', label = "transmitted")

# plt.plot([0,60,60,90,90,x[-1]], [1000,1000,500,500,1000,1000], label = "link", color = 'k')

plt.title(fig_name, fontsize = 20)
plt.grid()
plt.legend()

if len(sys.argv) == 3 and sys.argv[2] == 'save':
    plt.savefig(fig_name + '.png')
else:
    plt.show()
