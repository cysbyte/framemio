import test from 'node:test';
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {EcsTaskProtectionClient} from './ecs-task-protection.js';

test('enables and disables ECS task scale-in protection', async () => {
  const requests: Array<{method: string | undefined; url: string | undefined; body: unknown}> = [];
  const server = createServer((request, response) => {
    let body = '';
    request.setEncoding('utf8');
    request.on('data', (chunk) => {
      body += chunk;
    });
    request.on('end', () => {
      requests.push({method: request.method, url: request.url, body: JSON.parse(body)});
      response.writeHead(200, {'Content-Type': 'application/json'});
      response.end('{}');
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address !== 'string');

  try {
    const client = new EcsTaskProtectionClient(`http://127.0.0.1:${address.port}/`, 120, 1000);
    assert.equal(await client.protect(), true);
    await client.unprotect();
  } finally {
    await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  }

  assert.deepEqual(requests, [
    {
      method: 'PUT',
      url: '/task-protection/v1/state',
      body: {ProtectionEnabled: true, ExpiresInMinutes: 120},
    },
    {
      method: 'PUT',
      url: '/task-protection/v1/state',
      body: {ProtectionEnabled: false},
    },
  ]);
});

test('reports task protection as unavailable outside ECS', async () => {
  const client = new EcsTaskProtectionClient('');
  assert.equal(await client.protect(), false);
  await client.unprotect();
});
