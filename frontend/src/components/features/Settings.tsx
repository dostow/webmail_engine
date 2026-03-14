import { useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/label';
import './Settings.css';

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
    <div className="settings-view">
      <Card>
        <div className="card-header">
          <div className="card-title-wrapper">
            <h3 className="card-title">API Configuration</h3>
          </div>
        </div>
        <div className="card-content">
          <div className="settings-form">
            <div className="form-group">
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
              <div className="text-sm text-success mt-2">Configuration saved!</div>
            )}
          </div>
        </div>
      </Card>

      <Card>
        <div className="card-header">
          <div className="card-title-wrapper">
            <h3 className="card-title">About</h3>
          </div>
        </div>
        <div className="card-content">
          <div className="about-section">
            <h4>Webmail Engine Frontend</h4>
            <p className="about-version">Version 1.0.0</p>
            <p className="about-description">
              A modern React-based frontend for the Webmail Engine API.
              Built with Vite, TypeScript, and React.
            </p>
          </div>
        </div>
      </Card>
    </div>
  );
}
