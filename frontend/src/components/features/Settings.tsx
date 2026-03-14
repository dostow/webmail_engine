import { useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/label';

export function SettingsView() {
  const [apiUrl, setApiUrl] = useState(
    localStorage.getItem('apiUrl') || 'http://localhost:8080'
  );
  const [saved, setSaved] = useState(false);

  const handleSave = () => {
    localStorage.setItem('apiUrl', apiUrl);
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  return (
    <div className="flex flex-col gap-6 max-w-[600px]">
      <Card>
        <div className="flex items-center justify-between border-b px-6 py-4">
          <h3 className="text-lg font-semibold">API Configuration</h3>
        </div>
        <div className="p-6">
          <div className="flex flex-col gap-4">
            <div>
              <Label htmlFor="apiUrl">API Base URL</Label>
              <Input
                id="apiUrl"
                type="url"
                value={apiUrl}
                onChange={(e) => setApiUrl(e.target.value)}
                placeholder="http://localhost:8080"
              />
              <p className="text-sm text-muted-foreground mt-1">
                This URL is stored locally in your browser
              </p>
            </div>
            <Button variant="default" onClick={handleSave}>
              Save Configuration
            </Button>
            {saved && (
              <div className="text-sm text-green-600 mt-2">Configuration saved!</div>
            )}
          </div>
        </div>
      </Card>

      <Card>
        <div className="flex items-center justify-between border-b px-6 py-4">
          <h3 className="text-lg font-semibold">About</h3>
        </div>
        <div className="p-6">
          <div className="text-center">
            <h4 className="text-xl font-semibold mb-2">Webmail Engine Frontend</h4>
            <p className="text-sm text-muted-foreground mb-4">Version 1.0.0</p>
            <p className="text-sm text-muted-foreground leading-relaxed">
              A modern React-based frontend for the Webmail Engine API.
              Built with Vite, TypeScript, and React.
            </p>
          </div>
        </div>
      </Card>
    </div>
  );
}
