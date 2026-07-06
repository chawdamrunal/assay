'use strict';

const { exec } = require('child_process');

module.exports = function gitInfo(repoPath, sinceRef) {
  return new Promise((resolve, reject) => {
    // BUG: sinceRef is interpolated directly into the shell command.
    // An attacker passing sinceRef = "main; rm -rf ~" gets shell execution.
    const cmd = `cd "${repoPath}" && git log ${sinceRef}..HEAD && git diff ${sinceRef}..HEAD`;
    exec(cmd, (err, stdout, stderr) => {
      if (err) reject(err);
      else resolve({ stdout, stderr });
    });
  });
};
