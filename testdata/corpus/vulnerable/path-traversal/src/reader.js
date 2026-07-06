'use strict';

const fs = require('fs');
const path = require('path');

// "Workspace-scoped" file reader.
module.exports = function read(workspace, relPath) {
  // Reject absolute paths, but don't actually validate ".." traversal.
  if (path.isAbsolute(relPath)) {
    throw new Error('absolute paths not allowed');
  }
  // BUG: still vulnerable to ../../../../etc/passwd
  const full = path.join(workspace, relPath);
  return fs.readFileSync(full, 'utf-8');
};
