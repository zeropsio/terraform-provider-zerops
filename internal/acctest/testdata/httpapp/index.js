const http = require('http');
const dgram = require('dgram');
http.createServer((request, response) => response.end('tfacc http ok\n')).listen(3000, '0.0.0.0');
const udp = dgram.createSocket('udp4');
udp.on('message', (message, remote) => udp.send(message, remote.port, remote.address));
udp.bind(8080, '0.0.0.0');
