import { describe, it, expect } from 'vitest';
import { api } from '../../src/api';

// For now, since api relies on fetch and localStorage which are present in the browser
// we'll just test that the functions are defined correctly in api.js

describe('API Client', () => {
  it('should export all required methods', () => {
    expect(typeof api.health).toBe('function');
    expect(typeof api.providers).toBe('function');
    expect(typeof api.models).toBe('function');
    expect(typeof api.combos).toBe('function');
  });

  it('should have proxy pool methods', () => {
    expect(typeof api.proxyPools).toBe('function');
    expect(typeof api.createProxyPool).toBe('function');
  });

  it('should have mitm methods', () => {
    expect(typeof api.mitmStatus).toBe('function');
  });

  it('should have cli tool methods', () => {
    expect(typeof api.cliToolSettings).toBe('function');
  });
});
