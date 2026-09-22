const smooth = (a, b, value) => {
  const t = Math.min(1, Math.max(0, (value - a) / (b - a)));
  return t * t * (3 - 2 * t);
};

export function createFlyReaction() {
  let kind = 'idle', startedAt = -Infinity;
  const idle = () => ({ kind: 'idle', age: 0, envelope: 0, x: 0, y: 0, z: 0, yaw: 0, pitch: 0, speed: 0.012, clap: 0, offended: 0 });
  return {
    trigger(next, now) { kind = next === 'reward' ? 'reward' : 'correction'; startedAt = now; },
    reset() { kind = 'idle'; startedAt = -Infinity; },
    sample(now, reduceMotion = false) {
      if (reduceMotion || kind === 'idle') return idle();
      const t = Math.max(0, now - startedAt);
      const duration = kind === 'reward' ? 4.6 : 7.2;
      if (t >= duration) { kind = 'idle'; return idle(); }
      if (kind === 'reward') {
        const envelope = smooth(0, 0.38, t) * (1 - smooth(3.55, 4.6, t));
        return { kind, age: t, envelope, x: 0, y: 0.19 * envelope, z: 0,
          yaw: 0, pitch: 0.38 * envelope, speed: 0, clap: envelope, offended: 0 };
      }
      const envelope = smooth(0, 0.25, t) * (1 - smooth(6.5, 7.2, t));
      const outbound = smooth(1.0, 3.0, t), returning = smooth(4.9, 7.0, t);
      const distance = outbound * (1 - returning);
      const recoil = smooth(0, 0.16, t) * (1 - smooth(0.22, 0.65, t));
      const walking = (t > 1.0 && t < 3.0) || (t > 4.9 && t < 7.0);
      return { kind, age: t, envelope,
        x: -0.96 * distance - 0.07 * recoil, y: -0.065 * envelope + (walking ? Math.sin(t * 17) * 0.012 : 0),
        z: -0.48 * distance,
        yaw: 2.85 * smooth(0.3, 1.1, t) * (1 - smooth(4.45, 5.1, t)),
        pitch: -0.12 * envelope, speed: walking ? 0.72 : t < 1.1 ? 0.16 : 0.012,
        clap: 0, offended: envelope };
    },
  };
}

