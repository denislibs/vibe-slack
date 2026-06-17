import type { Component } from "solid-js";
import s from "./Avatar.module.css";

// Deterministic background colour derived from a name.
export function colorForName(name: string): string {
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = (hash * 31 + name.charCodeAt(i)) | 0;
  }
  const hue = ((hash % 360) + 360) % 360;
  return `hsl(${hue} 55% 45%)`;
}

function initial(name: string): string {
  const ch = name.trim().charAt(0);
  return ch ? ch.toUpperCase() : "?";
}

export type AvatarProps = {
  name: string;
  size?: number;
};

// Square initials avatar with a stable per-name colour.
export const Avatar: Component<AvatarProps> = (props) => {
  const size = () => props.size ?? 36;
  return (
    <div
      class={s.avatar}
      style={{
        width: `${size()}px`,
        height: `${size()}px`,
        background: colorForName(props.name),
        "font-size": `${Math.round(size() * 0.45)}px`,
      }}
    >
      {initial(props.name)}
    </div>
  );
};
