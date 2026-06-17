function bytesToB64(b: Uint8Array): string {
  let s = "";
  for (const x of b) s += String.fromCharCode(x);
  return btoa(s);
}

export interface CryptoLike {
  signingPublicKey(): Promise<Uint8Array>;
  keyPackage(): Promise<Uint8Array>;
}
export interface EnrollLike {
  enrollDevice(token: string, signingPublicKey: string, label: string, initialKeyPackages: string[]): Promise<string>;
}
export interface OnboardArgs {
  crypto: CryptoLike;
  enroll: EnrollLike;
  token: string;
  label: string;
  poolSize: number;
}

// Generates the device's signing key + a pool of one-time KeyPackages and registers
// the device with the AS, binding it to the session. Returns the new device id.
export async function onboardDevice(args: OnboardArgs): Promise<string> {
  const pub = bytesToB64(await args.crypto.signingPublicKey());
  const packages: string[] = [];
  for (let i = 0; i < args.poolSize; i++) {
    packages.push(bytesToB64(await args.crypto.keyPackage()));
  }
  return args.enroll.enrollDevice(args.token, pub, args.label, packages);
}
