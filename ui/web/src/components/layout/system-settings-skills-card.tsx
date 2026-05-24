import { Upload } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface SystemSettingsSkillsCardProps {
  uploadMaxSize: string;
  setUploadMaxSize: (value: string) => void;
}

export function SystemSettingsSkillsCard({
  uploadMaxSize,
  setUploadMaxSize,
}: SystemSettingsSkillsCardProps) {
  const { t } = useTranslation("system-settings");

  return (
    <Card className="border-sky-200 dark:border-sky-800">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <Upload className="h-4 w-4 text-sky-500" />
          {t("skills.title")}
        </CardTitle>
        <CardDescription>{t("skills.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-2 pt-0">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-0.5">
            <Label htmlFor="skillUploadMaxSize" className="text-sm font-medium">
              {t("skills.maxUploadSize")}
            </Label>
            <p className="text-xs text-muted-foreground">{t("skills.maxUploadSizeHint")}</p>
          </div>
          <Input
            id="skillUploadMaxSize"
            type="number"
            min={1}
            max={500}
            value={uploadMaxSize}
            onChange={(e) => setUploadMaxSize(e.target.value)}
            className="w-24 shrink-0 text-base md:text-sm"
          />
        </div>
      </CardContent>
    </Card>
  );
}
