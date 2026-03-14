import { InputHTMLAttributes } from 'react';
import { Input } from './Input';
import { Label } from './label';

interface LabeledInputProps extends InputHTMLAttributes<HTMLInputElement> {
  label: string;
  error?: string;
}

export function LabeledInput({ label, error, id, className, ...props }: LabeledInputProps) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <Input id={id} className={className} {...props} />
      {error && <p className="text-sm text-destructive">{error}</p>}
    </div>
  );
}
